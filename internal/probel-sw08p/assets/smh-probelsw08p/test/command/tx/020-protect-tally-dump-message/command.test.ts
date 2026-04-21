import * as _ from 'lodash';
import { ProtectTallyDumpCommand } from '../../../../src/command/tx/020-protect-tally-dump-message/command';
import { ProtectTallyDumpCommandParams } from '../../../../src/command/tx/020-protect-tally-dump-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import { ProtectTallyDumpCommandItems } from '../../../../src/command/tx/020-protect-tally-dump-message/items';

describe('Protect Tally Dump Message', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            // generate an array of ProtectTallyDumpCommandItems
            const buildDataArray = new Array<ProtectTallyDumpCommandItems>();
            for (let nbrCmdToSend = 0; nbrCmdToSend < 400; nbrCmdToSend++) {
                for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                    // Add the Protect Tally Dump Command Item buffer to the array
                    // {deviceId: value, protectDetails: value}
                    buildDataArray.push({ deviceId: itemIndex * 4, protectedData: itemIndex });
                }
            }

            const params: ProtectTallyDumpCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstDestinationId: 63,
                numberOfProtectTallies: 64,
                deviceNumberProtectDataItems: buildDataArray
            };
            // Act
            const metaCommand = new ProtectTallyDumpCommand(params);

            // Assert
            expect(metaCommand).toBeDefined();
            expect(metaCommand.params).toBe(params);
            expect(metaCommand.identifier).toBe(CommandIdentifiers.TX.GENERAL.PROTECT_TALLY_DUMP_MESSAGE);
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            // generate an array of ProtectTallyDumpCommandItems
            const buildDataArray = new Array<ProtectTallyDumpCommandItems>();
            for (let nbrCmdToSend = 0; nbrCmdToSend < 400; nbrCmdToSend++) {
                for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                    // Add the Protect Tally Dump Command Item buffer to the array
                    // {deviceId: value, protectDetails: value}
                    buildDataArray.push({ deviceId: -1, protectedData: -1 });
                }
            }

            const params: ProtectTallyDumpCommandParams = {
                matrixId: -1,
                levelId: -1,
                firstDestinationId: -1,
                numberOfProtectTallies: 0,
                deviceNumberProtectDataItems: buildDataArray
            };
            // Act
            const fct = () => new ProtectTallyDumpCommand(params);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.numberOfProtectTallies.id).toBe(
                    CommandErrorsKeys.NUMBER_OF_PROTECT_TALLIES_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.destinationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.deviceNumberProtectDataItems.id).toBe(
                    CommandErrorsKeys.DEVICE_NUMBER_AND_PROTECT_DETAILS_ARE_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            // generate an array of ProtectTallyDumpCommandItems
            const buildDataArray = new Array<ProtectTallyDumpCommandItems>();
            for (let nbrCmdToSend = 0; nbrCmdToSend < 400; nbrCmdToSend++) {
                for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                    // Add the Protect Tally Dump Command Item buffer to the array
                    // {deviceId: value, protectDetails: value}
                    buildDataArray.push({ deviceId: 1024, protectedData: 6 });
                }
            }

            const params: ProtectTallyDumpCommandParams = {
                matrixId: 256,
                levelId: 256,
                firstDestinationId: 65536,
                numberOfProtectTallies: 65,
                deviceNumberProtectDataItems: buildDataArray
            };
            // Act
            const fct = () => new ProtectTallyDumpCommand(params);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.numberOfProtectTallies.id).toBe(
                    CommandErrorsKeys.NUMBER_OF_PROTECT_TALLIES_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.destinationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.deviceNumberProtectDataItems.id).toBe(
                    CommandErrorsKeys.DEVICE_NUMBER_AND_PROTECT_DETAILS_ARE_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('Protect Tally Dump Message CMD_020_0X14', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.GENERAL.PROTECT_TALLY_DUMP_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                // generate an array of ProtectTallyDumpCommandItems
                const buildDataArray = new Array<ProtectTallyDumpCommandItems>();
                for (let nbrCmdToSend = 0; nbrCmdToSend < 32; nbrCmdToSend++) {
                    for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                        // Add the Protect Tally Dump Command Item buffer to the array
                        // {deviceId: value, protectDetails: value}
                        buildDataArray.push({ deviceId: itemIndex * 4, protectedData: itemIndex });
                    }
                }

                const params: ProtectTallyDumpCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    firstDestinationId: 0,
                    numberOfProtectTallies: 64,
                    deviceNumberProtectDataItems: buildDataArray
                };

                // Act
                const metaCommand = new ProtectTallyDumpCommand(params);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            '14 00 40 00 00 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c',
                        bytesCount: 0x85,
                        checksum: 0xa7,
                        buffer:
                            '10 02 14 00 40 00 00 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 85 a7 10 03'
                    },
                    {
                        data:
                            '14 00 40 00 40 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c',
                        bytesCount: 0x85,
                        checksum: 0x67,
                        buffer:
                            '10 02 14 00 40 00 40 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 85 67 10 03'
                    }
                ]);
            });
        });

        describe('Extended Protect Tally Dump Message CMD_148_0X94', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.EXTENDED.PROTECT_TALLY_DUMP_MESSAGE;
            });
            it('Should create & pack the extended command (...)', () => {
                // Arrange
                // generate an array of ProtectTallyDumpCommandItems
                const buildDataArray = new Array<ProtectTallyDumpCommandItems>();
                for (let nbrCmdToSend = 0; nbrCmdToSend < 32; nbrCmdToSend++) {
                    for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                        // Add the Protect Tally Dump Command Item buffer to the array
                        // {deviceId: value, protectDetails: value}
                        buildDataArray.push({ deviceId: itemIndex * 4, protectedData: itemIndex });
                    }
                }

                const params: ProtectTallyDumpCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    firstDestinationId: 896,
                    numberOfProtectTallies: 64,
                    deviceNumberProtectDataItems: buildDataArray
                };

                // Act
                const metaCommand = new ProtectTallyDumpCommand(params);
                metaCommand.buildCommand();


                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            '94 00 00 40 03 80 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c',
                        bytesCount: 0x86,
                        checksum: 0xa3,
                        buffer:
                            '10 02 94 00 00 40 03 80 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 86 a3 10 03'
                    },
                    {
                        data:
                            '94 00 00 40 03 c0 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c 00 00 10 04 20 08 30 0c',
                        bytesCount: 0x86,
                        checksum: 0x63,
                        buffer:
                            '10 02 94 00 00 40 03 c0 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 00 00 10 10 04 20 08 30 0c 86 63 10 03'
                    }
                ]);
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.EXTENDED.PROTECT_TALLY_DUMP_MESSAGE;
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                // generate an array of ProtectTallyDumpCommandItems
                const buildDataArray = new Array<ProtectTallyDumpCommandItems>();
                for (let nbrCmdToSend = 0; nbrCmdToSend < 8; nbrCmdToSend++) {
                    for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                        // Add the Protect Tally Dump Command Item buffer to the array
                        // {deviceId: value, protectDetails: value}
                        buildDataArray.push({ deviceId: 16, protectedData: itemIndex });
                    }
                }

                const params: ProtectTallyDumpCommandParams = {
                    matrixId: 16,
                    levelId: 16,
                    firstDestinationId: 16,
                    numberOfProtectTallies: 16,
                    deviceNumberProtectDataItems: buildDataArray
                };

                // Act
                const metaCommand = new ProtectTallyDumpCommand(params);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            '94 10 10 10 00 10 00 10 10 10 20 10 30 10 00 10 10 10 20 10 30 10 00 10 10 10 20 10 30 10 00 10 10 10 20 10 30 10',
                        bytesCount: 0x26,
                        checksum: 0x86,
                        buffer:
                            '10 02 94 10 10 10 10 10 10 00 10 10 00 10 10 10 10 10 10 20 10 10 30 10 10 00 10 10 10 10 10 10 20 10 10 30 10 10 00 10 10 10 10 10 10 20 10 10 30 10 10 00 10 10 10 10 10 10 20 10 10 30 10 10 26 86 10 03'
                    },
                    {
                        data:
                            '94 10 10 10 00 20 00 10 10 10 20 10 30 10 00 10 10 10 20 10 30 10 00 10 10 10 20 10 30 10 00 10 10 10 20 10 30 10',
                        bytesCount: 0x26,
                        checksum: 0x76,
                        buffer:
                            '10 02 94 10 10 10 10 10 10 00 20 00 10 10 10 10 10 10 20 10 10 30 10 10 00 10 10 10 10 10 10 20 10 10 30 10 10 00 10 10 10 10 10 10 20 10 10 30 10 10 00 10 10 10 10 10 10 20 10 10 30 10 10 26 76 10 03'
                    }
                ]);
            });
        });
    });

    describe('to Log Description of the general command', () => {
        it('Should log General command description', () => {
            // Arrange
            // generate an array of ProtectTallyDumpCommandItems
            const buildDataArray = new Array<ProtectTallyDumpCommandItems>();
            for (let nbrCmdToSend = 0; nbrCmdToSend < 32; nbrCmdToSend++) {
                for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                    // Add the Protect Tally Dump Command Item buffer to the array
                    // {deviceId: value, protectDetails: value}
                    buildDataArray.push({ deviceId: 16, protectedData: itemIndex });
                }
            }

            const params: ProtectTallyDumpCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstDestinationId: 0,
                numberOfProtectTallies: 64,
                deviceNumberProtectDataItems: buildDataArray
            };
            // Act
            const metaCommand = new ProtectTallyDumpCommand(params);
            const description = metaCommand.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
    describe('to Log Description of the general command', () => {
        it('Should log Extended general command description', () => {
            // Arrange
            // generate an array of ProtectTallyDumpCommandItems
            const buildDataArray = new Array<ProtectTallyDumpCommandItems>();
            for (let nbrCmdToSend = 0; nbrCmdToSend < 32; nbrCmdToSend++) {
                for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                    // Add the Protect Tally Dump Command Item buffer to the array
                    // {deviceId: value, protectDetails: value}
                    buildDataArray.push({ deviceId: 16, protectedData: itemIndex });
                }
            }

            const params: ProtectTallyDumpCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstDestinationId: 896,
                numberOfProtectTallies: 64,
                deviceNumberProtectDataItems: buildDataArray
            };
            // Act
            const metaCommand = new ProtectTallyDumpCommand(params);
            const description = metaCommand.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
});
