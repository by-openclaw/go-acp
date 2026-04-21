import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandParamsUtility, CrossPointConnectMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the CrossPoint Connect Message command
 *
 * This message is issued by the remote device in order to set crosspoints.
 * The controller will respond with a CROSSPOINT CONNECTED message (Command Byte 04).
 *
 * Command issued by the remote device
 *  * <uml>
 *     Youssef->Eric : hello
 * </uml>
 *
 * @export
 * @class CrossPointConnectMessageCommand
 * @extends {CommandBase<CrossPointConnectMessageCommandParams>}
 */
export class CrossPointConnectMessageCommand extends CommandBase<CrossPointConnectMessageCommandParams, any> {
    /**
     * Creates an instance of CrossPointConnectMessageCommand
     *
     * @param {CrossPointConnectMessageCommandParams} params the command parameters
     * @memberof CrossPointConnectMessageCommand
     */
    constructor(params: CrossPointConnectMessageCommandParams) {
        super(CrossPointConnectMessageCommand.getCommandId(params), params);
        this.validateParams(params);
    }

    /**
     * Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false => matrixId and LevelId are 4 bits coded and the multiplier of (destinationId / 128) must be smaller than 7 (3 bits coded)
     * + Extended : true
     *
     * @private
     * @static
     * @param {CrossPointConnectMessageCommandParams} params the command parameters
     * @returns {boolean} 'true' if the command is extended otherwise 'false'
     * @memberof CrossPointConnectMessageCommand
     */
    private static isExtended(params: CrossPointConnectMessageCommandParams): boolean {
        return params.matrixId > 15 || params.levelId > 15 || params.destinationId > 895 || params.sourceId > 1023;
    }

    /**
     * Gets the Command Identifier based on the state of isExtended
     *
     * @private
     * @static
     * @param {CrossPointConnectMessageCommandParams} params the command parameters
     * @returns {number} the command identifier
     * @memberof CrossPointConnectMessageCommand
     */
    private static getCommandId(params: CrossPointConnectMessageCommandParams): CommandIdentifier {
        return CrossPointConnectMessageCommand.isExtended(params)
            ? CommandIdentifiers.RX.EXTENDED.CROSSPOINT_CONNECT_MESSAGE
            : CommandIdentifiers.RX.GENERAL.CROSSPOINT_CONNECT_MESSAGE;
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof CrossPointConnectMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} -   ${this.name}: ${CommandParamsUtility.toString(this.params)}`;

        return CrossPointConnectMessageCommand.isExtended(this.params)
            ? descriptionFor('Extended')
            : descriptionFor('General');
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof CrossPointConnectMessageCommand
     */
    protected buildData(): Buffer {
        return CrossPointConnectMessageCommand.isExtended(this.params)
            ? this.buildDataExtended()
            : this.buildDataNormal();
    }

    /**
     * Validates the command parameters and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {CrossPointConnectMessageCommandParams} params the command parameters
     * @memberof CrossPointConnectMessageCommand
     */
    private validateParams(params: CrossPointConnectMessageCommandParams): void {
        const validationErrors: Record<string, LocaleData> = new CommandParamsValidator(params).validate();

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }

    /**
     * Builds the General command
     * Returns Probel SW-P-08 - General CrossPoint Connect Message CMD_002_0X02
     *
     * + This message is issued by the remote device in order to set crosspoints.
     * + The controller will respond with a CROSSPOINT CONNECTED message (Command Byte 04).
     *
     * | Message | Command Byte | 002 - 0x02                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  |              | Matrix/Level number                                                                                                                |
     * |         | Bits[4-7]    | Matrix Number                                                                                                                      |
     * |         | Bits[0-3]    | Level Number                                                                                                                       |
     * | Byte 2  | Multiplier   | this field allows sources and dests of up to 1023 to be used and provides source status info from a TDM or HD Digital Video router |
     * |         |              | 0                                                                                                                                  |
     * |         | Bit[7]       | Dest number DIV 128                                                                                                                |
     * |         | Bits[4-6]    | TDM and Digital Video source "bad" status (0 = good source)                                                                        |
     * |         | Bit[3]       | Source number DIV 128                                                                                                              |
     * |         | Bits[0-2]    |                                                                                                                                    |
     * | Byte 3  | Dest number  | Destination number MOD 128                                                                                                         |
     * | Byte 4  | Src  number  | Source number MOD 128                                                                                                              |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof CrossPointConnectMessageCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 5 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(this.params.matrixId, this.params.levelId))
            .writeUInt8(BufferUtility.combine2BytesMultiplierMsbLsb(this.params.destinationId, this.params.sourceId))
            .writeUInt8(this.params.destinationId % 128)
            .writeUInt8(this.params.sourceId % 128)
            .toBuffer();
    }

    /**
     * Builds the extended command
     * Returns Probel SW-P-08 - Extended General CrossPoint Connect Message CMD_130_0X82
     *
     * + This message is issued by the remote device in order to set crosspoints.
     * + The controller will respond with an EXTENDED CONNECTED message (Command Byte 132).
     *
     * | Message |  Command Byte   | 130 - 0x82                                                                                                                      |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format    | Notes                                                                                                                           |
     * | Byte 1  | Matrix number   |                                                                                                                                 |
     * | Byte 2  | Level number    |                                                                                                                                 |
     * | Byte 3  | Dest multiplier | Destination number DIV 256                                                                                                      |
     * | Byte 4  | Dest number     | Destination number MOD 256                                                                                                      |
     * | Byte 5  | Src multiplier  | Source number DIV 256                                                                                                           |
     * | Byte 6  | Src number      | Source number MOD 256                                                                                                           |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof CrossPointConnectMessageCommand
     */
    private buildDataExtended(): Buffer {
        const buffer = new SmartBuffer({ size: 7 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.matrixId)
            .writeUInt8(this.params.levelId)
            .writeUInt8(Math.floor(this.params.destinationId / 256))
            .writeUInt8(this.params.destinationId % 256)
            .writeUInt8(Math.floor(this.params.sourceId / 256))
            .writeUInt8(this.params.sourceId % 256);
        return buffer.toBuffer();
    }
}
