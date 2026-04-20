import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandOptionsUtility, CrossPointGroupSalvoTallyCommandOptions } from './options';
import { CommandParamsUtility, CrossPointGroupSalvoTallyCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the CrossPoint Group Salvo Tally Message command
 *
 * This message is issued by the controller in response to a CROSSPOINT GROUP SALVO INTERROGATE message (Command Byte 124).
 * It returns the SALVO data for the specified SALVO GROUP.
 * The data returned is for a particular index, the validity flag indicates whether any more data is present.
 *
 * N.B. : The group salvo commands are only implemented on the XD and ECLIPSE router ranges.
 *
 * Command issued by the remote device
 * @export
 * @class CrossPointGroupSalvoTallyCommand
 * @extends {CommandBase<CrossPointGroupSalvoTallyCommandParams>}
 */
export class CrossPointGroupSalvoTallyCommand extends CommandBase<
    CrossPointGroupSalvoTallyCommandParams,
    CrossPointGroupSalvoTallyCommandOptions
> {
    /**
     *Creates an instance of CrossPointGroupSalvoTallyCommand.
     * @param {CrossPointGroupSalvoTallyCommandParams} params the command parameters
     * @param {CrossPointGroupSalvoTallyCommandOptions} _options the command options
     * @memberof CrossPointGroupSalvoTallyCommand
     */
    constructor(params: CrossPointGroupSalvoTallyCommandParams, options: CrossPointGroupSalvoTallyCommandOptions) {
        super(CrossPointGroupSalvoTallyCommand.getCommandId(params), params, options);
        this.validateParams(params);
    }
    /**
     * Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false => matrixId, levelId are 4 bits coded destination < 895 ConnectIndex > 65535 and sourceId > 1023
     * + Extended : true
     *
     * @private
     * @static
     * @param {CrossPointGroupSalvoTallyCommandParams} params the command parameters
     * @returns {boolean} 'true' if the command is extended otherwise 'false'
     * @memberof CrossPointGroupSalvoTallyCommand
     */
    private static isExtended(params: CrossPointGroupSalvoTallyCommandParams): boolean {
        return (
            params.matrixId > 15 ||
            params.levelId > 15 ||
            params.destinationId > 895 ||
            params.sourceId > 1023 ||
            params.connectIndex > 255
        );
    }
    /**
     * Gets the Command Identifier based on the state of isExtended
     *
     * @private
     * @static
     * @param {CrossPointGroupSalvoTallyCommandParams} params the command parameters
     * @returns {number} the command identifier
     * @memberof CrossPointGroupSalvoTallyCommand
     */
    private static getCommandId(params: CrossPointGroupSalvoTallyCommandParams): CommandIdentifier {
        return CrossPointGroupSalvoTallyCommand.isExtended(params)
            ? CommandIdentifiers.TX.EXTENDED.CROSSPOINT_GROUP_SALVO_TALLY_MESSAGE
            : CommandIdentifiers.TX.GENERAL.CROSSPOINT_GROUP_SALVO_TALLY_MESSAGE;
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof ProtectDiscConnectedCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}, ${CommandOptionsUtility.toString(
                this.options
            )}`;

        return CrossPointGroupSalvoTallyCommand.isExtended(this.params)
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
        return CrossPointGroupSalvoTallyCommand.isExtended(this.params)
            ? this.buildDataExtended()
            : this.buildDataNormal();
    }

    /**
     * Validates the command parameters, options and throws a ValidationError in error(s) occur
     * @private
     * @param {CrossPointGroupSalvoTallyCommandParams} params command parameters
     * @param {CrossPointGroupSalvoTallyCommandOptions} options the command options
     * @memberof CrossPointGroupSalvoTallyCommand
     */
    private validateParams(params: CrossPointGroupSalvoTallyCommandParams): void {
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
     * Build the General command
     * Returns Probel SW-P-08 - CrossPoint Group Salvo Tally Message CMD_125_0X7d
     *
     * + This message is issued by the controller in response to a CROSSPOINT GROUP SALVO INTERROGATE message (Command Byte 124). It returns the SALVO data for the specified SALVO GROUP.
     * + The data returned is for a particular index, the validity flag indicates whether any more data is present.
     *
     * + N.B. : The group salvo commands are only implemented on the XD and ECLIPSE router ranges.
     *
     * | Message | Command Byte | 125 - 0x7d                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  | Matrix/Level | Matrix/Level number as defined in 3.1.2                                                                                            |
     * | Byte 2  | Multiplier   | Multiplier as defined in 3.1.2 (Except bad source bit always 0).                                                                   |
     * | Byte 3  | Dest num     | Destination number MOD 128                                                                                                         |
     * | Byte 4  | Srcs num     | Source number MOD 128                                                                                                              |
     * | Byte 5  | Salvo group  | Salvo group number as defined in 3.1.29                                                                                            |
     * | Byte 6  | Connect index| Connect index as defined in 3.1.31                                                                                                 |
     * | Byte 7  | Validity     | Validity flag                                                                                                                      |
     * |         | 0            | Valid connect index returned, more data available                                                                                  |
     * |         | 1            | Valid connect index returned, last in queue                                                                                        |
     * |         | 2            | Invalid connect (no data in SALVO                                                                                                  |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof CrossPointGroupSalvoTallyCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 8 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(this.params.matrixId, this.params.levelId))
            .writeUInt8(BufferUtility.combine2BytesMultiplierMsbLsb(this.params.destinationId, this.params.sourceId))
            .writeUInt8(this.params.destinationId % 128)
            .writeUInt8(this.params.sourceId % 128)
            .writeUInt8(this.params.salvoId)
            .writeUInt8(this.params.connectIndex)
            .writeUInt8(this.options.salvoValidityFlag)
            .toBuffer();
    }

    /**
     * Build the Extended command
     * Returns Probel SW-P-08 - Extended CrossPoint Group Salvo Tally Message CMD_253_0Xfd
     *
     * + This message is issued by the controller in response to a CROSSPOINT GROUP SALVO INTERROGATE message (Command Byte 124).
     * + It returns the SALVO data for the specified SALVO GROUP.
     * + The data returned is for a particular index, the validity flag indicates whether any more data is present.
     *
     * + N.B. : The group salvo commands are only implemented on the XD and ECLIPSE router ranges.
     *
     * | Message | Command Byte  | 253 - 0xfa                                                                                                                         |
     * |---------|---------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format  | Notes                                                                                                                              |
     * | Byte 1  | Matrix number |                                                                                                                                    |
     * | Byte 2  | Level number  |                                                                                                                                    |
     * | Byte 3  | Dest mult     | Destination number multiplier DIV 256                                                                                              |
     * | Byte 4  | Dest num      | Destination number MOD 256                                                                                                         |
     * | Byte 5  | Src mult      | Source number DIV 256                                                                                                              |
     * | Byte 6  | Src num       | Source number MOD 256                                                                                                              |
     * | Byte 7  | Salvo Group   | Salvo group number as defined in 3.1.29                                                                                            |
     * | Byte 8  | Connect index | Connect index DIV 256 as defined in 3.4.17                                                                                         |
     * | Byte 9  | Connect index | Connect index MOD 256 as defined in 3.4.17                                                                                         |
     * | Byte 10 | Validity flag | Validity flag                                                                                                                      |
     * |         | 0             | Valid connect index returned, more data available                                                                                  |
     * |         | 1             | Valid connect index returned, last in queue                                                                                        |
     * |         | 2             | Invalid connect (no data in SALVO                                                                                                  |
     *
     * @private
     * @returns {Buffer} Extended CrossPoint Group Salvo Tally Message
     * @memberof CrossPointGroupSalvoTallyCommand
     */
    private buildDataExtended(): Buffer {
        return new SmartBuffer({ size: 11 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.matrixId)
            .writeUInt8(this.params.levelId)
            .writeUInt8(Math.floor(this.params.destinationId / 256))
            .writeUInt8(this.params.destinationId % 256)
            .writeUInt8(Math.floor(this.params.sourceId / 256))
            .writeUInt8(this.params.sourceId % 256)
            .writeUInt8(this.params.salvoId)
            .writeUInt8(Math.floor(this.params.connectIndex / 256))
            .writeUInt8(this.params.connectIndex % 256)
            .writeUInt8(this.options.salvoValidityFlag)
            .toBuffer();
    }
}
