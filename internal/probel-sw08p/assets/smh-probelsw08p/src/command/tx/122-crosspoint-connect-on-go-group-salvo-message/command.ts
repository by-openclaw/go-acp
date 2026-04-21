import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandParamsUtility, CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the CrossPoint Connect On Go Group Salvo Acknowledge Message command
 *
 * This message is issued by the controller in response to a CROSSPOINT CONNECT ON GO GROUP SALVO message (Command Byte 120).
 *
 * Command issued by Pro-Bel Controller
 * @export
 * @class CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand
 * @extends {CommandBase<CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams>}
 */
export class CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand extends CommandBase<
    CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams,
    {}
> {
    /**
     * Creates an instance of CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand.
     * @param {CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams} params  the command parameters
     * @memberof CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand
     */
    constructor(params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams) {
        super(CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand.getCommandId(params), params);
        this.validateParams(params);
    }

    /**
     * Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false => matrixId and LevelId are 4 bits coded and the multiplier of (destinationId / 128) must be smaller than 7 (3 bits coded)
     * + Extended : true
     * @private
     * @static
     * @param {CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams} params the command parameters
     * @returns {boolean} 'true' if the command is extended otherwise 'false'
     * @memberof CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand
     */
    private static isExtended(params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams): boolean {
        // General Command is 4 bits = 16 range [0-15]
        if (params.matrixId > 15) {
            return true;
        }
        // 4 bits = 16 range [0-15]
        if (params.levelId > 15) {
            return true;
        } // two bytes
        if (params.destinationId > 895) {
            return true;
        }
        // two bytes
        if (params.sourceId > 1023) {
            return true;
        }
        return false;
    }

    /**
     * Gets the Command Identifier based on the state of isExtended
     * @private
     * @static
     * @param {CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams} params
     * @returns {number} Command ID of the command
     * @memberof CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand
     */
    private static getCommandId(
        params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams
    ): CommandIdentifier {
        return CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand.isExtended(params)
            ? CommandIdentifiers.TX.EXTENDED.CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_ACKNOWLEDGE_MESSAGE
            : CommandIdentifiers.TX.GENERAL.CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_ACKNOWLEDGE_MESSAGE;
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}`;

        return CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand.isExtended(this.params)
            ? descriptionFor('Extended')
            : descriptionFor('General');
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} Pro-Bel SW-P-8 command CrossPoint Connect On Go Group Salvo Acknowledge Message
     * @memberof CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand
     */
    protected buildData(): Buffer {
        return CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand.isExtended(this.params)
            ? this.buildDataExtended()
            : this.buildDataNormal();
    }

    /**
     * Validates the command parameters, options and throws a ValidationError in error(s) occur
     * @private
     * @param {CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams} params command parameters
     * @memberof ProtectConnectedCommand
     */
    private validateParams(params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams): void {
        const validationErrors: Record<string, LocaleData> = {
            ...new CommandParamsValidator(params).validate()
        };

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }

    /**
     * Builds the general command
     * Returns Probel SW-P-08 - CrossPoint Connect On Go Group Salvo Acknowledge Message CMD_122_0X7a
     *
     * + This message is issued by the controller in response to a CROSSPOINT CONNECT ON GO GROUP SALVO message (Command Byte 120).
     *
     * + N.B. : The group salvo commands are only implemented on the XD and ECLIPSE router ranges.
     *
     * | Message | Command Byte | 122 - 0x7a                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  |              | Matrix/Level number                                                                                                                |
     * |         | Bits[4-7]    | Matrix Number                                                                                                                      |
     * |         | Bits[0-3]    | Level Number                                                                                                                       |
     * | Byte 2  | Multiplier   | this field allows sources and dests of up to 1023 to be used and provides source status info from a TDM or HD Digital Video router |
     * |         | Bit[7]       | 0                                                                                                                                  |
     * |         | Bits[4-6]    | Dest number DIV 128                                                                                                                |
     * |         | Bit[3]       | 0                                                                                                                                  |
     * |         | Bits[0-2]    |Source number DIV 128                                                                                                               |
     * | Byte 3  | Dest number  | Destination number MOD 128                                                                                                         |
     * | Byte 4  | Src  number  | Source number MOD 128                                                                                                              |
     * | Byte 5  | Salvo number | Salvo number MOD 128                                                                                                               |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 6 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(this.params.matrixId, this.params.levelId))
            .writeUInt8(BufferUtility.combine2BytesMultiplierMsbLsb(this.params.destinationId, this.params.sourceId))
            .writeUInt8(this.params.destinationId % 128)
            .writeUInt8(this.params.sourceId % 128)
            .writeUInt8(this.params.salvoId % 128)
            .toBuffer();
    }

    /**
     * Build the extended command
     * Returns Probel SW-P-08 - Extended CrossPoint Connect On Go Group Salvo Acknowledge Message CMD_250_0Xfa
     *
     * + This message is issued by the controller in response to an EXTENDED  CROSSPOINT CONNECT ON GO GROUP SALVO message (Command Byte 248).
     *
     * + N.B. : The group salvo commands are only implemented on the XD and ECLIPSE router ranges.
     *
     * | Message |  Command Byte   | 250 - 0xfa                                                                                                                      |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format    | Notes                                                                                                                           |
     * | Byte 1  | Matrix number   |                                                                                                                                 |
     * | Byte 2  | Level number    |                                                                                                                                 |
     * | Byte 3  | Dest multiplier | Destination number DIV 256                                                                                                      |
     * | Byte 4  | Dest number     | Destination number MOD 256                                                                                                      |
     * | Byte 5  | Src multiplier  | Source number DIV 256                                                                                                           |
     * | Byte 6  | Src number      | Source number MOD 256                                                                                                           |
     * | Byte 7  | Salvo num       | Holds the Salvo group number to configure                                                                                       |
     * |         | Bit[7]          | 0                                                                                                                               |
     * |         | Bit[0-6]        | Salvo number 0-127                                                                                                              |
     * |         |                 | Destination and source will always overwrite previous data.                                                                     |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand
     */
    private buildDataExtended(): Buffer {
        return new SmartBuffer({ size: 8 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.matrixId)
            .writeUInt8(this.params.levelId)
            .writeUInt8(Math.floor(this.params.destinationId / 256))
            .writeUInt8(this.params.destinationId % 256)
            .writeUInt8(Math.floor(this.params.sourceId / 256))
            .writeUInt8(this.params.sourceId % 256)
            .writeUInt8(this.params.salvoId)
            .toBuffer();
    }
}
